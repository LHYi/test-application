# test-application

This is a test application to test the functions in a newly developed chaincode.

The application is designed to use Org1's credential files. It takes a string value indicating the function name and them ask for the number of parameters, then a slice of strings is passed to the chaincode as parameters.

Note: the application doesn't have the error handling ability. Thus, the chaincode function name and parameters (especially variable type) should be correct.

